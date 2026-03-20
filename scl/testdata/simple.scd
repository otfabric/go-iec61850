<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="TestSCL" version="1" revision="0"/>
  <Substation name="Sub1" desc="Test Substation">
    <VoltageLevel name="E1" desc="110kV">
      <Voltage unit="V" multiplier="k">110</Voltage>
      <Bay name="Q1" desc="Bay 1">
        <ConductingEquipment name="QA1" type="CBR" desc="Circuit Breaker"/>
        <ConductingEquipment name="QB1" type="DIS" desc="Disconnector"/>
      </Bay>
      <Bay name="Q2" desc="Bay 2">
        <ConductingEquipment name="QA2" type="CBR"/>
      </Bay>
    </VoltageLevel>
  </Substation>
  <Communication>
    <SubNetwork name="Net1" desc="Station Bus" type="8-MMS">
      <ConnectedAP iedName="IED1" apName="S1">
        <Address>
          <P type="IP">192.168.1.10</P>
          <P type="IP-SUBNET">255.255.255.0</P>
          <P type="IP-GATEWAY">192.168.1.1</P>
          <P type="OSI-PSEL">00000001</P>
          <P type="OSI-SSEL">0001</P>
          <P type="OSI-TSEL">0001</P>
        </Address>
        <GSE ldInst="LD1" cbName="gcb01">
          <Address>
            <P type="MAC-Address">01-0C-CD-01-00-01</P>
            <P type="APPID">0001</P>
            <P type="VLAN-ID">0</P>
            <P type="VLAN-PRIORITY">4</P>
          </Address>
          <MinTime unit="s" multiplier="m">10</MinTime>
          <MaxTime unit="s" multiplier="m">1000</MaxTime>
        </GSE>
        <SMV ldInst="LD1" cbName="smv01">
          <Address>
            <P type="MAC-Address">01-0C-CD-04-00-01</P>
            <P type="APPID">4000</P>
            <P type="VLAN-ID">0</P>
            <P type="VLAN-PRIORITY">4</P>
          </Address>
        </SMV>
      </ConnectedAP>
    </SubNetwork>
  </Communication>
  <IED name="IED1" desc="Test IED" manufacturer="TestMfr" type="TestType">
    <Services>
      <DynAssociation/>
      <GetDirectory/>
      <GetDataObjectDefinition/>
      <GetDataSetValue/>
      <DataSetDirectory/>
      <ConfDataSet max="10" maxAttributes="100" modify="true"/>
      <ReadWrite/>
      <ConfReportControl max="20" bufMode="both"/>
      <GetCBValues/>
      <ReportSettings cbName="Fix" datSet="Dyn" rptID="Dyn" optFields="Fix" bufTime="Dyn" trgOps="Dyn" intgPd="Dyn"/>
      <ConfLNs fixPrefix="true" fixLnInst="true"/>
      <GOOSE max="5" fixedOffs="false"/>
      <SMVsc max="3"/>
      <FileHandling/>
    </Services>
    <AccessPoint name="S1">
      <Server>
        <LDevice inst="LD1" desc="Logical Device 1">
          <LN0 lnClass="LLN0" inst="" lnType="LLN0_Type">
            <DataSet name="ds1" desc="Test dataset">
              <FCDA ldInst="LD1" prefix="" lnClass="GGIO" lnInst="1" doName="Ind1" daName="stVal" fc="ST"/>
              <FCDA ldInst="LD1" prefix="" lnClass="GGIO" lnInst="1" doName="Ind1" daName="q" fc="ST"/>
            </DataSet>
            <ReportControl name="brcb01" rptID="rpt01" datSet="ds1" confRev="1" buffered="true" bufTime="100" intgPd="5000">
              <TrgOps dchg="true" qchg="true" dupd="false" period="true" gi="true"/>
              <OptFields seqNum="true" timeStamp="true" dataSet="false" reasonCode="true" dataRef="false" entryID="true" configRef="true" bufOvfl="true"/>
            </ReportControl>
            <ReportControl name="urcb01" rptID="urpt01" datSet="ds1" confRev="1" buffered="false">
              <TrgOps dchg="true" qchg="false" dupd="false" period="false" gi="true"/>
              <OptFields seqNum="true" timeStamp="false" reasonCode="false"/>
            </ReportControl>
            <LogControl name="lcb01" desc="Test log"/>
          </LN0>
          <LN prefix="" lnClass="GGIO" inst="1" lnType="GGIO_Type" desc="Generic IO"/>
          <LN prefix="" lnClass="GGIO" inst="2" lnType="GGIO_Type"/>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0_Type" lnClass="LLN0" desc="LLN0 type">
      <DO name="Mod" type="INC_Mod"/>
      <DO name="Beh" type="INS_Beh"/>
    </LNodeType>
    <LNodeType id="GGIO_Type" lnClass="GGIO" desc="Generic IO type">
      <DO name="Ind1" type="SPS_Ind"/>
      <DO name="AnIn1" type="MV_AnIn"/>
    </LNodeType>
    <DOType id="INC_Mod" cdc="INC" desc="Controllable integer status">
      <DA name="stVal" fc="ST" bType="Enum" type="Mod_Enum"/>
      <DA name="q" fc="ST" bType="Quality"/>
      <DA name="t" fc="ST" bType="Timestamp"/>
      <DA name="ctlModel" fc="CF" bType="Enum" type="CtlModels"/>
    </DOType>
    <DOType id="INS_Beh" cdc="INS" desc="Integer status">
      <DA name="stVal" fc="ST" bType="INT32"/>
      <DA name="q" fc="ST" bType="Quality"/>
      <DA name="t" fc="ST" bType="Timestamp"/>
    </DOType>
    <DOType id="SPS_Ind" cdc="SPS" desc="Single point status">
      <DA name="stVal" fc="ST" bType="BOOLEAN"/>
      <DA name="q" fc="ST" bType="Quality"/>
      <DA name="t" fc="ST" bType="Timestamp"/>
    </DOType>
    <DOType id="MV_AnIn" cdc="MV" desc="Measured value">
      <DA name="mag" fc="MX" bType="Struct" type="AnalogueValue"/>
      <DA name="q" fc="MX" bType="Quality"/>
      <DA name="t" fc="MX" bType="Timestamp"/>
    </DOType>
    <DAType id="AnalogueValue" desc="Analogue value">
      <BDA name="f" bType="FLOAT32"/>
    </DAType>
    <EnumType id="Mod_Enum">
      <EnumVal ord="1">on</EnumVal>
      <EnumVal ord="2">blocked</EnumVal>
      <EnumVal ord="3">test</EnumVal>
      <EnumVal ord="4">test/blocked</EnumVal>
      <EnumVal ord="5">off</EnumVal>
    </EnumType>
    <EnumType id="CtlModels">
      <EnumVal ord="0">status-only</EnumVal>
      <EnumVal ord="1">direct-with-normal-security</EnumVal>
      <EnumVal ord="2">sbo-with-normal-security</EnumVal>
      <EnumVal ord="3">direct-with-enhanced-security</EnumVal>
      <EnumVal ord="4">sbo-with-enhanced-security</EnumVal>
    </EnumType>
  </DataTypeTemplates>
</SCL>

<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns="http://www.iec.ch/61850/2003/SCL" xsi:schemaLocation="http://www.iec.ch/61850/2003/SCL SCL.xsd" version="2007" revision="C" release="5">
	<Header id="SCL Example T1-1" toolID="MySystemTool" uuid="6dacf413-cb9e-4ce8-8a45-7c6325b1afaa" version="0" revision="1">
		<SourceFiles>
			<SclFileReference fileType="SSD" fileUuid="cef3a885-67d8-44d4-a739-71ebb6f62ad7" fileName="SCLExample.ssd" version="1" revision="0"/>
		</SourceFiles>
	</Header>
	<Substation name="S12" desc="Baden" uuid="13f64bc2-af68-4b3b-9cbb-7b7d96573ce4" templateUuid="85a60620-a521-47a9-9c06-f96b431894df">
		<Labels>
			<Label id="LocalDisplay" lang="en">Baden substation</Label>
			<Label id="LocalDisplay" lang="fr">Sous-station de Baden</Label>
			<Label id="DistantDisplay" lang="en">Distant Baden substation</Label>
			<Label id="DistantDisplay" lang="fr">Sous-station de Baden distante</Label>
		</Labels>
		<PowerTransformer name="T1" type="PTR" uuid="77981ec9-3e65-41b9-b1b1-7dddc52d03d0">
			<LNode lnInst="1" lnClass="PDIF" ldInst="F1" iedName="D1Q1BP2" uuid="007f3a68-0b38-42d2-b449-c898fdb84eb0" lnUuid="ac0c6367-309e-492c-b156-eb8eea31d456"/>
			<LNode lnInst="1" lnClass="YLTC" ldInst="S12D1T1" iedName="None" uuid="eab67dc9-2dc1-4472-b85d-67c883877722"/>
			<TransformerWinding name="W1" type="PTW" uuid="38825d37-7aa5-4b10-a4ca-2d0da9152b4b">
				<Terminal connectivityNode="S12/D1/Q1/L1" substationName="S12" voltageLevelName="D1" bayName="Q1" cNodeName="L1" cNodeUuid="19ca9be9-4c9c-40ae-a34d-2801f377f207"/>
			</TransformerWinding>
			<TransformerWinding name="W2" type="PTW" uuid="48e0dfb4-4d34-47b8-8f37-925698098667">
				<Terminal connectivityNode="S12/E1/Q2/L1" substationName="S12" voltageLevelName="E1" bayName="Q2" cNodeName="L1" cNodeUuid="b12d7f2b-565f-4ebe-ac3d-3eb08a51452c"/>
			</TransformerWinding>
		</PowerTransformer>
		<VoltageLevel name="D1" uuid="7bd2e298-5f0e-4678-98ca-44d2bb0a96e8">
			<Voltage multiplier="k" unit="V">220</Voltage>
			<Bay name="Q1" uuid="08ed6657-7d79-4783-b99b-253bcd8df8d9">
				<LNode iedName="None" ldInst="S12D1Q1" lnClass="PDIS" lnInst="1" uuid="138b9c93-b03f-4da3-b7b1-3ff8315b05d4"/>
				<ConductingEquipment name="I1" type="CTR" uuid="dbd7c820-b7f3-47e1-8f98-e54895e38078">
					<Terminal connectivityNode="S12/D1/Q1/L1" substationName="S12" voltageLevelName="D1" bayName="Q1" cNodeName="L1" cNodeUuid="19ca9be9-4c9c-40ae-a34d-2801f377f207"/>
					<Terminal connectivityNode="S12/D1/Q1/L2" substationName="S12" voltageLevelName="D1" bayName="Q1" cNodeName="L2" cNodeUuid="b4cffa30-5bca-4987-9b33-c52efa424ee6"/>
					<SubEquipment name="R" phase="A" uuid="db0adbb7-a570-4c6a-8c88-d7a923729b12">
						<LNode iedName="D1Q1BP2" ldInst="F1" lnClass="TCTR" lnInst="1" uuid="f41365d6-4ee9-4b9b-a9c4-10ea1f6b8589" lnUuid="6e64e486-8781-4fb5-84d5-e0eb86957edd"/>
					</SubEquipment>
					<SubEquipment name="S" phase="B" uuid="9b1e39b9-4d16-43f5-a096-47924ed96415">
						<LNode iedName="D1Q1BP2" ldInst="F1" lnClass="TCTR" lnInst="2" uuid="02cfc5d3-bcea-4732-af94-07897a02d72a" lnUuid="8bff5373-801d-4fbc-a8a9-d6f36042fa98"/>
					</SubEquipment>
					<SubEquipment name="T" phase="C" uuid="3a693ccf-b45f-45c3-8469-97eb43eaf5ef">
						<LNode iedName="D1Q1BP2" ldInst="F1" lnClass="TCTR" lnInst="3" uuid="7c5f67d0-b026-4981-a498-1f8ca12f658e" lnUuid="533bde1a-80b9-4583-815a-98a9bf786f71"/>
					</SubEquipment>
					<SubEquipment name="I0" phase="N" uuid="0590f3e5-5921-4de0-b39c-a70a9633a9c0">
						<LNode iedName="D1Q1BP2" ldInst="F1" lnClass="TCTR" lnInst="4" uuid="2000c7cb-dc9d-4b0c-b7ef-7c61958f3b2a" lnUuid="3e645c3e-d51f-465e-8054-be9a30099370"/>
					</SubEquipment>
				</ConductingEquipment>
				<ConductingEquipment name="Feeder1" type="IFL" uuid="d636335e-4ac0-4a30-8dc8-4d45f69477e0">
					<Terminal connectivityNode="S12/D1/Q1/L2" substationName="S12" voltageLevelName="D1" bayName="Q1" cNodeName="L2" cNodeUuid="b4cffa30-5bca-4987-9b33-c52efa424ee6"/>
				</ConductingEquipment>
				<ConnectivityNode name="L1" pathName="S12/D1/Q1/L1" uuid="19ca9be9-4c9c-40ae-a34d-2801f377f207"/>
				<ConnectivityNode name="L2" pathName="S12/D1/Q1/L2" uuid="b4cffa30-5bca-4987-9b33-c52efa424ee6"/>
			</Bay>
		</VoltageLevel>
		<VoltageLevel name="E1" uuid="d8a872e8-13a0-4a15-ad11-337d1af52ae2" templateUuid="16320c9e-09fd-4397-bf4c-03b6bdba1ddd">
			<Voltage multiplier="k" unit="V">132</Voltage>
			<Bay name="Q1" uuid="fcf4beee-2a8d-4dda-99eb-006852453875" templateUuid="6f6210ef-cbdd-4ad4-a217-9c1a33bd0ad6">
				<LNode iedName="E1Q1SB1" ldInst="C1" lnClass="MMXN" lnInst="1" uuid="36120f11-41f4-4029-8e41-0fe8c9d9e8a2" lnUuid="f7a6c511-0552-4052-8b52-1a5b91566c6f"/>
				<LNode iedName="None" ldInst="S12E1Q1" lnClass="PDIS" lnInst="1" uuid="e954e868-ccca-43f7-bc52-b0cc1bb3b4a2"/>
				<LNode iedName="None" ldInst="F1" lnClass="PDIF" lnInst="1" uuid="fbfe235c-99ef-416c-99b3-d03008183821"/>
				<ConductingEquipment name="QA1" type="CBR" uuid="24a2fb28-fd24-41b0-9d04-b9bcf6199020" templateUuid="f4abe8a3-cfc1-4d1e-9108-5085e4519c8d">
					<LNode iedName="E1Q1SB1" ldInst="C1" lnClass="CSWI" lnInst="1" uuid="f74de021-0d8b-4132-b7cb-470153a34103" lnUuid="5a2f9a89-573a-439a-acab-8db6ba22b4fe"/>
					<Terminal connectivityNode="S12/E1/Q1/L1" substationName="S12" voltageLevelName="E1" bayName="Q1" cNodeName="L1" cNodeUuid="432ba848-bf65-4873-a2f3-c27f22b89a99"/>
					<Terminal connectivityNode="S12/E1/Q1/L2" substationName="S12" voltageLevelName="E1" bayName="Q1" cNodeName="L2" cNodeUuid="79aaf7b2-e820-4df8-bd4f-3dd3376f3bba"/>
				</ConductingEquipment>
				<ConductingEquipment name="QB1" type="DIS" uuid="2c5e01fb-f1d2-401b-b84b-9a055d4b88ee" templateUuid="341b1561-4167-4ca7-b2b4-36e5f30b3ede">
					<LNode iedName="E1Q1SB1" ldInst="C1" lnClass="CSWI" lnInst="2" uuid="88946a27-f676-41ca-b269-67db551d1c75" lnUuid="e166c1da-8186-4993-bda7-836d5f864d9c"/>
					<LNode iedName="D1Q1SB4" ldInst="C1" lnClass="CILO" lnInst="1" uuid="9cbf7c5c-b583-4ab5-b8cd-eb5613c01c39" lnUuid="3a302f9c-4d15-4819-8bdd-f4e56cf588e1"/>
					<Terminal connectivityNode="S12/E1/Q4/B1" substationName="S12" voltageLevelName="E1" bayName="Q4" cNodeName="B1" cNodeUuid="be05f0e9-8d9d-4c20-a38d-a989cb1e7214"/>
					<Terminal connectivityNode="S12/E1/Q1/L1" substationName="S12" voltageLevelName="E1" bayName="Q1" cNodeName="L1" cNodeUuid="432ba848-bf65-4873-a2f3-c27f22b89a99"/>
				</ConductingEquipment>
				<ConductingEquipment name="U1" type="VTR" uuid="c1e453b1-a331-48ae-bc1f-afbcb2cee70c" templateUuid="97287bd5-fa2d-4d07-8780-8a0f68640bc9">
					<Terminal connectivityNode="S12/E1/Q1/L2" substationName="S12" voltageLevelName="E1" bayName="Q1" cNodeName="L2" cNodeUuid="79aaf7b2-e820-4df8-bd4f-3dd3376f3bba"/>
					<SubEquipment name="A" phase="A">
						<LNode iedName="E1Q1SB1" ldInst="C1" lnClass="TVTR" lnInst="1" uuid="2cdb906b-78c5-4841-94c6-36a4315542b9" lnUuid="97464472-b865-4613-8add-cfece8bd3418"/>
					</SubEquipment>
				</ConductingEquipment>
				<ConductingEquipment name="I1" type="CTR" uuid="c672365d-c419-4f48-9c71-1a3eb11bf660" templateUuid="8c99d1ff-b03e-4f66-b885-c3e48e280d75">
					<Terminal connectivityNode="S12/E1/Q1/L3" substationName="S12" voltageLevelName="E1" bayName="Q1" cNodeName="L3" cNodeUuid="287f68fc-a9c3-487b-850f-6b229dfa614b"/>
					<Terminal connectivityNode="S12/E1/Q1/L2" substationName="S12" voltageLevelName="E1" bayName="Q1" cNodeName="L2" cNodeUuid="79aaf7b2-e820-4df8-bd4f-3dd3376f3bba"/>
				</ConductingEquipment>
				<ConductingEquipment name="Feeder1" type="IFL" uuid="b557abe2-6823-42bd-853a-28dbd16b07a5" templateUuid="cbcfa980-9f87-4cba-8732-220470348105">
					<Terminal connectivityNode="S12/E1/Q1/L3" substationName="S12" voltageLevelName="E1" bayName="Q1" cNodeName="L3" cNodeUuid="287f68fc-a9c3-487b-850f-6b229dfa614b"/>
				</ConductingEquipment>
				<ConnectivityNode name="L1" pathName="S12/E1/Q1/L1" uuid="432ba848-bf65-4873-a2f3-c27f22b89a99"/>
				<ConnectivityNode name="L2" pathName="S12/E1/Q1/L2" uuid="79aaf7b2-e820-4df8-bd4f-3dd3376f3bba"/>
				<ConnectivityNode name="L3" pathName="S12/E1/Q1/L3" uuid="287f68fc-a9c3-487b-850f-6b229dfa614b"/>
			</Bay>
			<Bay name="Q2" desc="Turgi" uuid="b89ab08e-dacc-4405-ba5b-576abe36b603" templateUuid="5a036d52-9922-4140-a690-408474d63e9b">
				<ConductingEquipment name="QA1" type="CBR" uuid="30e32204-5812-4487-8ad0-8f0316b8f7d8" templateUuid="1098c929-4a76-47f2-91e9-af81181490ed">
					<LNode iedName="D1Q1SB4" ldInst="C1" lnClass="CSWI" lnInst="1" uuid="5c638d8f-b119-4138-ad50-55944290d994" lnUuid="b5fbf7b2-0e28-4556-bcb3-5372bab1fb2b"/>
					<Terminal connectivityNode="S12/E1/Q2/L2" substationName="S12" voltageLevelName="E1" bayName="Q2" cNodeName="L2" cNodeUuid="7452cf38-63ff-4fc0-978a-4403e08720c4"/>
					<Terminal connectivityNode="S12/E1/Q2/L3" substationName="S12" voltageLevelName="E1" bayName="Q2" cNodeName="L3" cNodeUuid="d0cff5cd-c325-4d7b-988e-ab11e4f54264"/>
				</ConductingEquipment>
				<ConductingEquipment name="QB1" type="DIS" uuid="47ce7dbc-5837-49e8-92be-35888c92a1d1" templateUuid="07afadda-409b-412c-907e-17d88d13e63d">
					<LNode iedName="D1Q1SB4" ldInst="C1" lnClass="CSWI" lnInst="2" uuid="5506049e-ce33-4f5c-a57e-a3e8215af43c" lnUuid="5fd008d1-0855-47ee-a512-dceb817c3452"/>
					<LNode iedName="D1Q1SB4" ldInst="C1" lnClass="CILO" lnInst="2" uuid="e44badb6-b415-4d68-95c1-841da84a1258" lnUuid="730d1bd1-1cc9-4aed-9be7-6ec4695bd4a6"/>
					<Terminal connectivityNode="S12/E1/Q4/B1" substationName="S12" voltageLevelName="E1" bayName="Q4" cNodeName="B1" cNodeUuid="be05f0e9-8d9d-4c20-a38d-a989cb1e7214"/>
					<Terminal connectivityNode="S12/E1/Q2/L3" substationName="S12" voltageLevelName="E1" bayName="Q2" cNodeName="L3" cNodeUuid="d0cff5cd-c325-4d7b-988e-ab11e4f54264"/>
				</ConductingEquipment>
				<ConductingEquipment name="I1" type="CTR" uuid="f4744325-89a0-43b8-8400-aee3ffd5c9df" templateUuid="4569f29c-9966-4bd5-84b5-d92aedfe0e95">
					<Terminal connectivityNode="S12/E1/Q2/L1" substationName="S12" voltageLevelName="E1" bayName="Q2" cNodeName="L1" cNodeUuid="b12d7f2b-565f-4ebe-ac3d-3eb08a51452c"/>
					<Terminal connectivityNode="S12/E1/Q2/L2" substationName="S12" voltageLevelName="E1" bayName="Q2" cNodeName="L2" cNodeUuid="7452cf38-63ff-4fc0-978a-4403e08720c4"/>
				</ConductingEquipment>
				<ConductingEquipment name="U1" type="VTR" uuid="ac25a768-7775-44a6-b4d9-8531495e6ae6" templateUuid="1c531489-4d3f-4b4c-a517-a78cc7d8ff2a">
					<Terminal connectivityNode="S12/E1/Q2/L1" substationName="S12" voltageLevelName="E1" bayName="Q2" cNodeName="L1" cNodeUuid="b12d7f2b-565f-4ebe-ac3d-3eb08a51452c"/>
				</ConductingEquipment>
				<ConnectivityNode name="L1" pathName="S12/E1/Q2/L1" uuid="b12d7f2b-565f-4ebe-ac3d-3eb08a51452c"/>
				<ConnectivityNode name="L2" pathName="S12/E1/Q2/L2" uuid="7452cf38-63ff-4fc0-978a-4403e08720c4"/>
				<ConnectivityNode name="L3" pathName="S12/E1/Q2/L3" uuid="d0cff5cd-c325-4d7b-988e-ab11e4f54264"/>
			</Bay>
			<Bay name="Q3" desc="London" uuid="e270daea-9acb-4303-8c60-50f231cbc843" templateUuid="5b7105c9-1315-4596-9f1f-fcaab48e1b7d">
				<LNode iedName="None" ldInst="LD0" lnClass="MMXN" lnInst="1" uuid="8f9504f2-4ef4-4b0a-b2d7-a6ff5f443dc4"/>
				<LNode iedName="None" ldInst="LD0" lnClass="PDIS" lnInst="1" uuid="d0974a27-2a47-4072-96ec-87aedc431781"/>
				<LNode iedName="None" ldInst="LD0" lnClass="PDIF" lnInst="1" uuid="cbf4af5a-a935-4cbc-8350-483d44345079"/>
				<ConductingEquipment name="QA1" type="CBR" uuid="04f5e079-d7f7-448e-b0bf-4e51210f9fac" templateUuid="f91d0778-e15c-4c77-bd1d-b679d9524314">
					<LNode iedName="None" ldInst="C1" lnClass="CSWI" lnInst="1" uuid="e90f8f25-c3c5-4264-a395-5daf06b17392"/>
					<Terminal connectivityNode="S12/E1/Q3/L1" substationName="S12" voltageLevelName="E1" bayName="Q3" cNodeName="L1" cNodeUuid="ec31bde4-266a-4e76-9309-d6fe65172340"/>
					<Terminal connectivityNode="S12/E1/Q3/L2" substationName="S12" voltageLevelName="E1" bayName="Q3" cNodeName="L2" cNodeUuid="373f7450-f276-47bf-8c10-c5b6f6bef544"/>
				</ConductingEquipment>
				<ConductingEquipment name="QB1" type="DIS" uuid="0c9ea886-e611-4d32-80ca-e4aa6896e72c" templateUuid="a447d75b-3f49-40d6-9f84-78afdc91ee54">
					<Terminal connectivityNode="S12/E1/Q4/B1" substationName="S12" voltageLevelName="E1" bayName="Q4" cNodeName="B1" cNodeUuid="be05f0e9-8d9d-4c20-a38d-a989cb1e7214"/>
					<Terminal connectivityNode="S12/E1/Q3/L1" substationName="S12" voltageLevelName="E1" bayName="Q3" cNodeName="L1" cNodeUuid="ec31bde4-266a-4e76-9309-d6fe65172340"/>
				</ConductingEquipment>
				<ConductingEquipment name="U1" type="VTR" uuid="5474df2f-b043-4ee0-bd8d-112f52714c73" templateUuid="df70fd15-94cc-421c-b1f8-8463108e70f8">
					<Terminal connectivityNode="S12/E1/Q3/L2" substationName="S12" voltageLevelName="E1" bayName="Q3" cNodeName="L2" cNodeUuid="373f7450-f276-47bf-8c10-c5b6f6bef544"/>
				</ConductingEquipment>
				<ConductingEquipment name="I1" type="CTR" uuid="c805bbfa-b229-4f6e-a23f-93e672471511" templateUuid="e1f5c6d3-25d8-42e4-9265-0497d55150b9">
					<Terminal connectivityNode="S12/E1/Q3/L3" substationName="S12" voltageLevelName="E1" bayName="Q3" cNodeName="L3" cNodeUuid="d70f43ad-a3e4-42ef-aca5-a2dfa1afc3aa"/>
					<Terminal connectivityNode="S12/E1/Q3/L2" substationName="S12" voltageLevelName="E1" bayName="Q3" cNodeName="L2" cNodeUuid="373f7450-f276-47bf-8c10-c5b6f6bef544"/>
				</ConductingEquipment>
				<ConductingEquipment name="Feeder1" type="IFL" uuid="df5fe89a-d0ea-497c-825b-1f199384319f" templateUuid="78a5840b-af12-4ba5-ae22-0564f2dbf8e2">
					<Terminal connectivityNode="S12/E1/Q3/L3" substationName="S12" voltageLevelName="E1" bayName="Q3" cNodeName="L3" cNodeUuid="d70f43ad-a3e4-42ef-aca5-a2dfa1afc3aa"/>
				</ConductingEquipment>
				<ConnectivityNode name="L1" pathName="S12/E1/Q3/L1" uuid="ec31bde4-266a-4e76-9309-d6fe65172340"/>
				<ConnectivityNode name="L2" pathName="S12/E1/Q3/L2" uuid="373f7450-f276-47bf-8c10-c5b6f6bef544"/>
				<ConnectivityNode name="L3" pathName="S12/E1/Q3/L3" uuid="d70f43ad-a3e4-42ef-aca5-a2dfa1afc3aa"/>
			</Bay>
			<Bay name="Q4" uuid="196d380f-2d41-4887-b539-52c705aa37b2" templateUuid="148d3d2d-9a0e-4913-ae8a-3c61ad13895d">
				<ConnectivityNode name="B1" pathName="S12/E1/Q4/B1" uuid="be05f0e9-8d9d-4c20-a38d-a989cb1e7214"/>
			</Bay>
		</VoltageLevel>
	</Substation>
	<Communication>
		<SubNetwork name="W01" type="8-MMS" uuid="176659a3-3c06-413b-b595-9d0e2a1c062e">
			<Text>Station bus</Text>
			<BitRate unit="b/s">100</BitRate>
			<ConnectedAP iedName="D1Q1SB4" apName="S1" apUuid="513d767b-410a-46c6-9afa-f9cea591768e">
				<Address>
					<P type="IP">10.0.0.11</P>
					<P type="IP-SUBNET">255.255.255.0</P>
					<P type="IP-GATEWAY">10.0.0.101</P>
					<P type="OSI-TSEL">00000001</P>
					<P type="OSI-PSEL">01</P>
					<P type="OSI-SSEL">01</P>
				</Address>
				<GSE ldInst="C1" cbName="SyckResult" cbUuid="8066999f-ff5f-4d0f-842d-977d0a43bec2">
					<Address>
						<P type="MAC-Address">01-0C-CD-01-00-02</P>
						<P type="APPID">3001</P>
						<P type="VLAN-PRIORITY">4</P>
					</Address>
					<MinTime unit="s">4</MinTime>
					<MaxTime unit="s">1000</MaxTime>
				</GSE>
				<PhysConn type="Connection">
					<P type="Type">FOC</P>
					<P type="Plug">ST</P>
				</PhysConn>
			</ConnectedAP>
			<ConnectedAP iedName="E1Q1SB1" apName="S1" apUuid="64cf6be2-155e-4d74-b731-7578e74704c7">
				<Address>
					<P type="IP">10.0.0.1</P>
					<P type="IP-SUBNET">255.255.255.0</P>
					<P type="IP-GATEWAY">10.0.0.101</P>
					<P type="OSI-TSEL">00000001</P>
					<P type="OSI-PSEL">01</P>
					<P type="OSI-SSEL">01</P>
				</Address>
				<GSE ldInst="C1" cbName="ItlPositions" cbUuid="e39667ca-e306-47e2-adf6-67c8cf56be60">
					<Address>
						<P type="MAC-Address">01-0C-CD-01-00-01</P>
						<P type="APPID">3000</P>
						<P type="VLAN-PRIORITY">4</P>
					</Address>
				</GSE>
				<SMV ldInst="C1" cbName="Volt" cbUuid="695bbe00-5fef-4088-8b06-5923e499c5fd">
					<Address>
						<P type="MAC-Address">01-0C-CD-04-00-01</P>
						<P type="APPID">4000</P>
						<P type="VLAN-ID">123</P>
						<P type="VLAN-PRIORITY">4</P>
					</Address>
				</SMV>
			</ConnectedAP>
			<ConnectedAP iedName="D1Q1BP2" apName="S1" apUuid="d7bc23bf-20bb-4c83-bde9-5aa0223c6506">
				<Address>
					<P type="IP">10.0.0.2</P>
					<P type="IP-SUBNET">255.255.255.0</P>
					<P type="IP-GATEWAY">10.0.0.101</P>
					<P type="OSI-TSEL">00000001</P>
					<P type="OSI-PSEL">01</P>
					<P type="OSI-SSEL">01</P>
				</Address>
			</ConnectedAP>
			<ConnectedAP iedName="D1Q1BP3" apName="S1" apUuid="b53a7998-f150-4b25-9eab-e65b9b9dc85e">
				<Address>
					<P type="IP">10.0.0.3</P>
					<P type="IP-SUBNET">255.255.255.0</P>
					<P type="IP-GATEWAY">10.0.0.101</P>
					<P type="OSI-TSEL">00000001</P>
					<P type="OSI-PSEL">01</P>
					<P type="OSI-SSEL">01</P>
				</Address>
			</ConnectedAP>
			<ConnectedAP iedName="A1KA1" apName="S1" apUuid="aab025f4-5eca-4e49-a17a-e471ddf80ff6">
				<Address>
					<P type="IP">10.0.0.121</P>
					<P type="IP-SUBNET">255.255.255.0</P>
					<P type="IP-GATEWAY">10.0.0.101</P>
					<P type="OSI-TSEL">00000001</P>
					<P type="OSI-PSEL">01</P>
					<P type="OSI-SSEL">01</P>
				</Address>
			</ConnectedAP>
			<ConnectedAP iedName="E1Q2SB1" apName="S1" apUuid="4c3fca73-77f5-4820-85b3-ebcfd03f90ed">
				<Address>
					<P type="IP">10.0.0.14</P>
					<P type="IP-SUBNET">255.255.255.0</P>
					<P type="IP-GATEWAY">10.0.0.101</P>
					<P type="OSI-TSEL">00000001</P>
					<P type="OSI-PSEL">01</P>
					<P type="OSI-SSEL">01</P>
				</Address>
			</ConnectedAP>
		</SubNetwork>
	</Communication>
	<IED name="E1Q1SB1" uuid="84e558dd-49df-427b-a9b5-178c5ca4aabe" templateUuid="9c54c325-3610-4d4e-91f9-b5d2fab26293" originalSclVersion="2007" originalSclRevision="C" originalSclRelease="4">
		<Services nameLength="64">
			<ClientServices goose="true" sv="true">
				<Security ACSEAuthentication="true" E2ESecurity="true"/>
			</ClientServices>
			<DynAssociation/>
			<GetDirectory/>
			<GetDataObjectDefinition/>
			<GetDataSetValue/>
			<DataSetDirectory/>
			<ConfDataSet max="4" maxAttributes="50"/>
			<ReadWrite/>
			<ConfReportControl max="12"/>
			<GetCBValues/>
			<ConfLogControl max="1"/>
			<ReportSettings cbName="Conf" datSet="Conf" rptID="Dyn" optFields="Conf" bufTime="Dyn" intgPd="Dyn"/>
			<GSESettings cbName="Conf" datSet="Conf" appID="Conf"/>
			<GOOSE max="2"/>
			<SMVSettings cbName="Conf" datSet="Conf" optFields="Fix" smpRate="Conf" svID="Conf">
				<SmpRate>80</SmpRate>
				<SmpRate>240</SmpRate>
			</SMVSettings>
			<FileHandling/>
			<ConfLNs fixLnInst="true"/>
		</Services>
		<AccessPoint name="S1" uuid="64cf6be2-155e-4d74-b731-7578e74704c7" templateUuid="33eebeab-9ecf-4115-ab9c-3156189528a7">
			<Server>
				<Authentication none="true"/>
				<LDevice inst="C1">
					<LN0 inst="" lnClass="LLN0" lnType="LN0" uuid="09a121bb-53e6-4e8a-a003-c173412d225e" templateUuid="ffb46a46-657c-406b-be9d-84b302684620">
						<DataSet name="Positions">
							<FCDA ldInst="C1" prefix="" lnClass="CSWI" lnInst="1" lnUuid="5a2f9a89-573a-439a-acab-8db6ba22b4fe" doName="Pos" fc="ST"/>
							<FCDA ldInst="C1" prefix="" lnClass="CSWI" lnInst="2" lnUuid="e166c1da-8186-4993-bda7-836d5f864d9c" doName="Pos" fc="ST"/>
						</DataSet>
						<DataSet name="Measurands">
							<FCDA ldInst="C1" prefix="" lnClass="MMXN" lnInst="1" lnUuid="f7a6c511-0552-4052-8b52-1a5b91566c6f" doName="Amp" fc="MX"/>
							<FCDA ldInst="C1" prefix="" lnClass="MMXN" lnInst="1" lnUuid="f7a6c511-0552-4052-8b52-1a5b91566c6f" doName="Vol" fc="MX"/>
						</DataSet>
						<DataSet name="smv">
							<FCDA ldInst="C1" prefix="" lnClass="TVTR" lnInst="1" lnUuid="97464472-b865-4613-8add-cfece8bd3418" doName="VolSv" daName="instMag" fc="MX"/>
						</DataSet>
						<ReportControl name="PosReport" rptID="E1Q1Switches" datSet="Positions" confRev="1" uuid="a0e0d63e-9209-4fb6-bea9-fa2663bd964c">
							<TrgOps dchg="true" qchg="true"/>
							<OptFields/>
							<RptEnabled max="5">
								<ClientLN iedName="A1KA1" apRef="S1" ldInst="LD0" lnInst="1" lnClass="IHMI" lnUuid="7884c2da-3e95-44d6-b586-c27c24eb93bb"/>
							</RptEnabled>
						</ReportControl>
						<ReportControl name="MeaReport" rptID="E1Q1Measurands" datSet="Measurands" confRev="1" uuid="114eaa19-48aa-4e12-beca-1792fd1e47db" intgPd="2000">
							<TrgOps qchg="true" period="true"/>
							<OptFields reasonCode="true"/>
							<RptEnabled max="5">
								<ClientLN iedName="A1KA1" apRef="S1" ldInst="LD0" lnInst="1" lnClass="IHMI" lnUuid="7884c2da-3e95-44d6-b586-c27c24eb93bb"/>
							</RptEnabled>
						</ReportControl>
						<LogControl name="Log" datSet="Positions" logName="C1" uuid="3d14d0ea-f9e9-4db8-8584-548860ccf228">
							<TrgOps dchg="true" qchg="true"/>
						</LogControl>
						<Log/>
						<GSEControl name="ItlPositions" datSet="Positions" appID="Itl" uuid="e39667ca-e306-47e2-adf6-67c8cf56be60">
							<IEDName apUuid="4c3fca73-77f5-4820-85b3-ebcfd03f90ed" apRef="S1">E1Q2SB1</IEDName>
						</GSEControl>
						<SampledValueControl name="Volt" datSet="smv" smvID="11" smpRate="4800" nofASDU="5" uuid="695bbe00-5fef-4088-8b06-5923e499c5fd">
							<IEDName apUuid="513d767b-410a-46c6-9afa-f9cea591768e" apRef="S1">D1Q1SB4</IEDName>
							<SmvOpts refreshTime="true" sampleSynchronized="true" sampleRate="true"/>
						</SampledValueControl>
					</LN0>
					<LN inst="1" lnClass="LPHD" lnType="LPHDa" uuid="f0025a93-7cab-48b8-a1df-d38b924f62b6" templateUuid="79c74d7f-08d2-485a-a939-e1307cd095d0">
						<DOI name="Proxy">
							<DAI name="stVal" valKind="Set">
								<Val>false</Val>
							</DAI>
						</DOI>
					</LN>
					<LN inst="1" lnClass="CSWI" lnType="CSWIa" uuid="5a2f9a89-573a-439a-acab-8db6ba22b4fe" templateUuid="54ca0dc3-d88b-401e-b8a2-9cfc209f3937">
						<Inputs>
							<ExtRef iedName="D1Q1SB4" ldInst="C1" lnClass="RSYN" lnInst="1" lnUuid="3185cecd-0e0f-46dd-a9ab-4119ea3fd014" doName="Rel" daName="stVal" pLN="RSYN" pDO="Rel" pDA="stVal" pServT="GOOSE" serviceType="GOOSE" srcCBName="SyckResult" srcCBUuid="8066999f-ff5f-4d0f-842d-977d0a43bec2"/>
						</Inputs>
					</LN>
					<LN inst="2" lnClass="CSWI" lnType="CSWIa" uuid="e166c1da-8186-4993-bda7-836d5f864d9c" templateUuid="415d33f1-d02d-441c-b0d4-9d5ef95fcbc8"/>
					<LN inst="1" lnClass="MMXN" lnType="MMXNa" uuid="f7a6c511-0552-4052-8b52-1a5b91566c6f" templateUuid="234bd0a7-f7ce-4134-8969-ddffd5da5c35">
						<DOI name="Vol">
							<SDI name="sVC">
								<DAI name="scaleFactor" valKind="Set">
									<Val>200</Val>
								</DAI>
								<DAI name="offset" valKind="Set">
									<Val>10</Val>
								</DAI>
							</SDI>
						</DOI>
					</LN>
					<LN inst="1" lnClass="TVTR" lnType="TVTRa" uuid="97464472-b865-4613-8add-cfece8bd3418" templateUuid="4fb240d6-ee83-4bf4-b1d5-496a5542cba5"/>
				</LDevice>
			</Server>
		</AccessPoint>
		<IEDSourceFiles>
			<SclFileReference fileType="ICD" fileUuid="e4ce4164-21e2-4bd6-95b1-528bba8b7fba" fileName="MyIED1.icd" version="1" revision="0"/>
			<SclFileReference fileType="IID" fileUuid="150e80fb-87b1-45d1-995c-da0d5e984f64" fileName="MyIED1.iid" version="1" revision="1"/>
		</IEDSourceFiles>
		<MinRequestedSCDFiles>
			<MinRequestedSCDFile fileType="SCD" fileUuid="cef3a885-67d8-44d4-a739-71ebb6f62ad7" version="1" revision="0"/>
		</MinRequestedSCDFiles>
	</IED>
	<IED name="E1Q1BP2" uuid="fc66351a-bc8a-4536-83b9-ee0e37054c1c" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="91577f38-6f8e-4f4a-a7bf-849cb3edbbc1"/>
	</IED>
	<IED name="E1Q1BP3" uuid="0bf1e0f2-3a8a-4385-8a6e-2729a504a03f" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="b25f585d-103b-4b59-b20b-c31e934c0a2e"/>
	</IED>
	<IED name="E1Q2SB1" uuid="c4dba5e2-5fef-408f-9ba3-b53e3797dea4" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="4c3fca73-77f5-4820-85b3-ebcfd03f90ed"/>
	</IED>
	<IED name="E1Q3SB1" uuid="eb996341-abaa-4893-927f-591b03de9ab0" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="32430341-588f-43b5-b343-1c02e24316a1"/>
	</IED>
	<IED name="E1Q3KA1" uuid="4b355641-8b15-48ce-84e6-415dd24ec838" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="d577d9ca-642c-4532-8199-27ed6f44486b"/>
	</IED>
	<IED name="E1Q3KA2" uuid="4959a27f-6340-4a7d-9e48-cf28a19cfa3c" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="f07ab12d-8349-4f14-8787-096c49ce9591"/>
	</IED>
	<IED name="E1Q3KA3" uuid="9688d155-f71c-4c6a-aec5-2a8591421d81" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="e2581283-5ac4-4cac-812e-e1a30ac6d6b0"/>
	</IED>
	<IED name="D1Q1SB1" uuid="f755c3dd-37ee-479c-a54c-d3fdd2e4d5f3" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="711b0e4c-c6d2-4235-acc1-a470c1b5d8a5"/>
	</IED>
	<IED name="D1Q1BP2" uuid="04cb18a3-66b5-427c-80fb-b30b021670e6" templateUuid="fc6f0168-449c-456f-aed9-c1c43ac1746d" originalSclVersion="2007" originalSclRevision="C" originalSclRelease="4">
		<Services nameLength="64">
			<DynAssociation/>
			<GetDirectory/>
			<GetDataObjectDefinition/>
			<GetDataSetValue/>
			<DataSetDirectory/>
			<ConfDataSet max="4"/>
			<ReadWrite/>
			<ConfReportControl max="12"/>
			<GetCBValues/>
			<ReportSettings cbName="Conf" datSet="Conf" rptID="Dyn" optFields="Dyn" bufTime="Dyn" intgPd="Dyn"/>
			<GSESettings cbName="Conf" datSet="Conf" appID="Conf"/>
			<GOOSE max="2"/>
			<FileHandling/>
		</Services>
		<AccessPoint name="S1" uuid="d7bc23bf-20bb-4c83-bde9-5aa0223c6506" templateUuid="47773728-8a4e-403d-ba17-afb1b7f3ef9c">
			<Server>
				<Authentication none="true"/>
				<LDevice inst="F1">
					<LN0 inst="" lnClass="LLN0" lnType="LN0" uuid="ea3f6e08-3abb-481d-a62a-ebfd099a29fb" templateUuid="0dafb60c-4b15-4ee2-8b4e-814d54245bb2"/>
					<LN inst="1" lnClass="LPHD" lnType="LPHDa" uuid="921fbb4a-13cb-406c-a884-d02480811fad" templateUuid="cc0a4f79-93e0-4709-94e4-a7470ea2e858">
						<DOI name="Proxy">
							<DAI name="stVal" valKind="Set">
								<Val>false</Val>
							</DAI>
						</DOI>
					</LN>
					<LN inst="1" lnClass="TCTR" lnType="TCTRa" uuid="6e64e486-8781-4fb5-84d5-e0eb86957edd" templateUuid="b9e0e4c9-80c7-40ae-b0ff-ff8ded157a0b"/>
					<LN inst="2" lnClass="TCTR" lnType="TCTRa" uuid="8bff5373-801d-4fbc-a8a9-d6f36042fa98" templateUuid="bce77eb6-3dc4-4b0b-98d3-118b0f09f0b0"/>
					<LN inst="3" lnClass="TCTR" lnType="TCTRa" uuid="533bde1a-80b9-4583-815a-98a9bf786f71" templateUuid="d7054b27-ab36-4fb0-a512-119574ea1fc7"/>
					<LN inst="4" lnClass="TCTR" lnType="TCTRa" uuid="3e645c3e-d51f-465e-8054-be9a30099370" templateUuid="2977c195-6a3b-45ef-b87c-d8f5595c60d6"/>
					<LN lnType="PDIFa" uuid="ac0c6367-309e-492c-b156-eb8eea31d456" templateUuid="280227cd-7a63-49e6-b77b-cb407b11ede0" inst="1" lnClass="PDIF">
						<DOI name="TmASt" desc="Example of array value definition – function wise meaningless">
							<SDI name="crvPts" ix="0">
								<DAI name="xVal">
									<Val>3.2</Val>
								</DAI>
								<DAI name="yVal">
									<Val>32.2</Val>
								</DAI>
							</SDI>
							<SDI name="crvPts" ix="1">
								<DAI name="xVal">
									<Val>12.5</Val>
								</DAI>
								<DAI name="yVal">
									<Val>22.1</Val>
								</DAI>
							</SDI>
							<SDI name="crvPts" ix="2">
								<DAI name="xVal">
									<Val>102.5</Val>
								</DAI>
								<DAI name="yVal">
									<Val>2.1</Val>
								</DAI>
							</SDI>
						</DOI>
					</LN>
				</LDevice>
			</Server>
		</AccessPoint>
		<IEDSourceFiles>
			<SclFileReference fileType="ICD" fileUuid="edb082d4-25c0-4b6b-8219-debd2189224e" fileName="MyIED2.icd" version="1" revision="0"/>
		</IEDSourceFiles>
		<MinRequestedSCDFiles>
			<MinRequestedSCDFile fileType="SCD" fileUuid="cef3a885-67d8-44d4-a739-71ebb6f62ad7" version="0" revision="1"/>
		</MinRequestedSCDFiles>
	</IED>
	<IED name="D1Q1BP3" uuid="456f06a0-ceb1-42ff-b265-089f5d454fb7" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="b53a7998-f150-4b25-9eab-e65b9b9dc85e"/>
	</IED>
	<IED name="D1Q1SB4" uuid="2fd8bbff-f90e-46f7-b049-30cd622af4df" templateUuid="d8ea4ec2-b7d7-49e6-aaeb-052b70ebc8c1" originalSclVersion="2007" originalSclRevision="C" originalSclRelease="4">
		<Services nameLength="64">
			<DynAssociation/>
			<GetDirectory/>
			<GetDataObjectDefinition/>
			<GetDataSetValue/>
			<DataSetDirectory/>
			<ConfDataSet max="4"/>
			<ReadWrite/>
			<ConfReportControl max="12"/>
			<GetCBValues/>
			<ConfLogControl max="1"/>
			<ReportSettings cbName="Conf" datSet="Conf" rptID="Dyn" optFields="Conf" bufTime="Dyn" intgPd="Dyn"/>
			<GSESettings cbName="Conf" datSet="Conf" appID="Conf"/>
			<GOOSE max="2"/>
			<FileHandling/>
		</Services>
		<AccessPoint name="S1" uuid="513d767b-410a-46c6-9afa-f9cea591768e" templateUuid="8043d460-59e6-41b4-9f54-7a695489374f">
			<Server>
				<Authentication none="true"/>
				<LDevice inst="C1">
					<LN0 inst="" lnClass="LLN0" lnType="LN0" uuid="2b7cc129-6d4f-478d-b0ad-0d80a36fffa8" templateUuid="40e0be68-7703-4df6-8dd7-67c71cf02d5c">
						<DataSet name="SyckResult">
							<FCDA ldInst="C1" prefix="" lnClass="RSYN" lnInst="1" lnUuid="3185cecd-0e0f-46dd-a9ab-4119ea3fd014" doName="Rel" fc="ST"/>
						</DataSet>
						<GSEControl name="SyckResult" datSet="SyckResult" confRev="1" appID="SynChk" uuid="8066999f-ff5f-4d0f-842d-977d0a43bec2">
							<IEDName apUuid="64cf6be2-155e-4d74-b731-7578e74704c7" apRef="S1">E1Q1SB1</IEDName>
						</GSEControl>
					</LN0>
					<LN inst="1" lnClass="LPHD" lnType="LPHDa" uuid="b8aa3917-863e-41ef-a496-af581eddd7a2" templateUuid="643feb36-4428-4c8e-9258-79061299cad0">
						<DOI name="Proxy">
							<DAI name="stVal" valKind="Set">
								<Val>false</Val>
							</DAI>
						</DOI>
					</LN>
					<LN inst="1" lnClass="RSYN" lnType="RSYNa" uuid="3185cecd-0e0f-46dd-a9ab-4119ea3fd014" templateUuid="8679b434-124e-41c6-820c-887de5d61cf3"/>
					<LN inst="1" lnClass="CILO" lnType="CILOa" uuid="3a302f9c-4d15-4819-8bdd-f4e56cf588e1" templateUuid="b0bd1305-028b-44c6-9527-932ded73df72"/>
					<LN inst="2" lnClass="CILO" lnType="CILOa" uuid="730d1bd1-1cc9-4aed-9be7-6ec4695bd4a6" templateUuid="1c8a0374-cd3e-44ee-9655-36597a513eec"/>
					<LN inst="1" lnClass="CSWI" lnType="CSWIa" uuid="b5fbf7b2-0e28-4556-bcb3-5372bab1fb2b" templateUuid="9db4a45f-27de-4bc1-8918-5761aed1f654"/>
					<LN inst="2" lnClass="CSWI" lnType="CSWIa" uuid="5fd008d1-0855-47ee-a512-dceb817c3452" templateUuid="19762548-fefb-446a-b59c-fabe5284f47e"/>
				</LDevice>
			</Server>
		</AccessPoint>
		<IEDSourceFiles>
			<SclFileReference fileType="ICD" fileUuid="082a6354-a5c4-4309-ae4d-860d75366741" fileName="MyIED3.icd" version="1" revision="0"/>
		</IEDSourceFiles>
		<MinRequestedSCDFiles>
			<MinRequestedSCDFile fileType="SCD" fileUuid="cef3a885-67d8-44d4-a739-71ebb6f62ad7" version="0" revision="1"/>
		</MinRequestedSCDFiles>
	</IED>
	<IED name="A1KA1" uuid="8f4f2b61-55b8-4aef-a529-055f20d3a051" originalSclVersion="2007" originalSclRevision="B" originalSclRelease="4">
		<AccessPoint name="S1" uuid="aab025f4-5eca-4e49-a17a-e471ddf80ff6">
			<LN inst="1" lnClass="IHMI" lnType="IHMIa" uuid="7884c2da-3e95-44d6-b586-c27c24eb93bb">
				<Outputs>
					<ExtCtrl intAddr="Ctrl1" pLN="CSWI" pDO="Pos" desc="Control of the breaker" uuid="f783ab82-0653-428b-abd6-2d0c2fb207a8" iedName="E1Q1SB1" apRef="S1" ldInst="C1" lnClass="CSWI" lnInst="1" doName="Pos" lnUuid="f0025a93-7cab-48b8-a1df-d38b924f62b6" checkSynchrocheck="false"/>
				</Outputs>
			</LN>
		</AccessPoint>
	</IED>
	<DataTypeTemplates>
		<LNodeType id="LN0" lnClass="LLN0">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLN0LPL"/>
		</LNodeType>
		<LNodeType id="LPHDa" lnClass="LPHD">
			<DO name="PhyNam" type="myDPL"/>
			<DO name="PhyHealth" type="myHealth"/>
			<DO name="Proxy" type="mySPS"/>
		</LNodeType>
		<LNodeType id="CSWIa" lnClass="CSWI">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLPL"/>
			<DO name="Pos" type="myPos"/>
		</LNodeType>
		<LNodeType id="MMXNa" lnClass="MMXN">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLPL"/>
			<DO name="Amp" type="myMV"/>
			<DO name="Vol" type="myMV"/>
		</LNodeType>
		<LNodeType id="CILOa" lnClass="CILO">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLPL"/>
			<DO name="EnaOpn" type="mySPS"/>
			<DO name="EnaCls" type="mySPS"/>
		</LNodeType>
		<LNodeType id="TVTRa" lnClass="TVTR">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLPL"/>
			<DO name="VolSv" type="mySAV"/>
			<DO name="FuFail" type="mySPS"/>
		</LNodeType>
		<LNodeType id="TCTRa" lnClass="TCTR">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLPL"/>
			<DO name="AmpSv" type="mySAV"/>
		</LNodeType>
		<LNodeType id="RSYNa" lnClass="RSYN">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLPL"/>
			<DO name="Rel" type="mySPS"/>
		</LNodeType>
		<LNodeType id="IHMIa" lnClass="IHMI">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLPL"/>
		</LNodeType>
		<LNodeType id="PDIFa" lnClass="PDIF">
			<DO name="Mod" type="myMod"/>
			<DO name="Beh" type="myBeh"/>
			<DO name="Health" type="myHealth"/>
			<DO name="NamPlt" type="myLPL"/>
			<DO name="Str" type="myACD"/>
			<DO name="Op" type="myACT"/>
			<DO name="TmASt" type="myCSD"/>
		</LNodeType>
		<DOType id="myMod" cdc="ENC">
			<DA name="stVal" fc="ST" dchg="true" bType="Enum" type="BehaviourModeKind"/>
			<DA name="q" fc="ST" qchg="true" bType="Quality"/>
			<DA name="t" fc="ST" bType="Timestamp"/>
			<DA name="ctlModel" fc="CF" bType="Enum" type="CtlModelKind" dchg="true"/>
			<DA name="Oper" fc="CO" bType="Struct" type="myModOper"/>
		</DOType>
		<DOType id="myHealth" cdc="ENS">
			<DA name="stVal" fc="ST" dchg="true" bType="Enum" type="HealthKind"/>
			<DA name="q" fc="ST" qchg="true" bType="Quality"/>
			<DA name="t" fc="ST" bType="Timestamp"/>
		</DOType>
		<DOType id="myBeh" cdc="ENS">
			<DA name="stVal" fc="ST" dchg="true" bType="Enum" type="BehaviourModeKind"/>
			<DA name="q" fc="ST" qchg="true" bType="Quality"/>
			<DA name="t" fc="ST" bType="Timestamp"/>
		</DOType>
		<DOType id="myINS" cdc="INS">
			<DA name="stVal" fc="ST" dchg="true" bType="INT32"/>
			<DA name="q" fc="ST" qchg="true" bType="Quality"/>
			<DA name="t" fc="ST" bType="Timestamp"/>
		</DOType>
		<DOType id="myLN0LPL" cdc="LPL">
			<DA name="vendor" fc="DC" bType="VisString255">
				<Val>myVendorName</Val>
			</DA>
			<DA name="swRev" fc="DC" bType="VisString255">
				<Val>my SW revision ID</Val>
			</DA>
			<DA name="d" fc="DC" bType="VisString255"/>
			<DA name="configRev" fc="DC" bType="VisString255">
				<Val>Rev 3.45</Val>
			</DA>
			<DA name="ldNs" fc="EX" bType="VisString255">
				<Val>IEC 61850-7-4:2007</Val>
			</DA>
		</DOType>
		<DOType id="myLPL" cdc="LPL">
			<DA name="vendor" fc="DC" bType="VisString255">
				<Val>myVendorName</Val>
			</DA>
			<DA name="swRev" fc="DC" bType="VisString255"/>
			<DA name="d" fc="DC" bType="VisString255"/>
		</DOType>
		<DOType id="myDPL" cdc="DPL">
			<DA name="vendor" fc="DC" bType="VisString255">
				<Val>myVendorName</Val>
			</DA>
			<DA name="hwRev" fc="DC" bType="VisString255">
				<Val>Rev 1.23</Val>
			</DA>
		</DOType>
		<DOType id="myPos" cdc="DPC">
			<DA name="stVal" fc="ST" dchg="true" bType="Dbpos"/>
			<DA name="q" fc="ST" qchg="true" bType="Quality"/>
			<DA name="t" fc="ST" bType="Timestamp"/>
			<DA name="ctlModel" fc="CF" bType="Enum" type="CtlModelKind" dchg="true"/>
			<DA name="Oper" fc="CO" bType="Struct" type="myOper"/>
			<DA name="SBOw" fc="CO" bType="Struct" type="myOper"/>
			<DA name="Cancel" fc="CO" bType="Struct" type="myCancel"/>
		</DOType>
		<DOType id="mySPS" cdc="SPS">
			<DA name="stVal" fc="ST" dchg="true" bType="BOOLEAN"/>
			<DA name="q" fc="ST" qchg="true" bType="Quality"/>
			<DA name="t" fc="ST" bType="Timestamp"/>
		</DOType>
		<DOType id="myMV" cdc="MV">
			<DA name="mag" fc="MX" dchg="true" bType="Struct" type="myAnalogValue"/>
			<DA name="q" fc="MX" qchg="true" bType="Quality"/>
			<DA name="t" fc="MX" bType="Timestamp"/>
			<DA name="sVC" fc="CF" dchg="true" bType="Struct" type="ScaledValueConfig"/>
		</DOType>
		<DOType id="myCMV" cdc="CMV">
			<DA name="cVal" fc="MX" dchg="true" bType="Struct" type="myVector"/>
			<DA name="q" fc="MX" qchg="true" bType="Quality"/>
			<DA name="t" fc="MX" bType="Timestamp"/>
		</DOType>
		<DOType id="mySEQ" cdc="SEQ">
			<SDO name="c1" type="myCMV"/>
			<SDO name="c2" type="myCMV"/>
			<SDO name="c3" type="myCMV"/>
			<DA name="seqT" fc="MX" bType="Enum" type="SequenceKind"/>
		</DOType>
		<DOType id="myACD" cdc="ACD">
			<DA name="general" fc="ST" dchg="true" bType="BOOLEAN"/>
			<DA name="dirGeneral" fc="ST" dchg="true" bType="Enum" type="FaultDirectionKind"/>
			<DA name="q" fc="ST" qchg="true" bType="Quality"/>
			<DA name="t" fc="ST" bType="Timestamp"/>
		</DOType>
		<DOType id="myACT" cdc="ACT">
			<DA name="general" fc="ST" dchg="true" bType="BOOLEAN"/>
			<DA name="q" fc="ST" qchg="true" bType="Quality"/>
			<DA name="t" fc="ST" bType="Timestamp"/>
		</DOType>
		<DOType id="myCSD" cdc="CSD">
			<DA name="xUnits" fc="DC" bType="Struct" type="myUnit"/>
			<DA name="xD" fc="DC" bType="VisString255"/>
			<DA name="yUnits" fc="DC" bType="Struct" type="myUnit"/>
			<DA name="yD" fc="DC" bType="VisString255"/>
			<DA name="numPts" fc="DC" bType="INT16U">
				<!-- only the first 3 indexes 0,1, and 2 of the 32 point array crvPts[0..31] are used in the curve description-->
				<Val>3</Val>
			</DA>
			<DA name="crvPts" fc="DC" count="maxPts" bType="Struct" type="xyPoint"/>
			<DA name="d" fc="DC" bType="VisString255"/>
			<DA name="maxPts" fc="CF" bType="INT16U" dchg="true">
				<Val>32</Val>
			</DA>
		</DOType>
		<DOType id="mySAV" cdc="SAV">
			<DA name="instMag" fc="MX" bType="Struct" type="myAnalogValue"/>
			<DA name="q" fc="MX" qchg="true" bType="Quality"/>
		</DOType>
		<DAType id="myAnalogValue">
			<BDA name="f" bType="FLOAT32"/>
		</DAType>
		<DAType id="ScaledValueConfig">
			<BDA name="scaleFactor" bType="FLOAT32"/>
			<BDA name="offset" bType="FLOAT32"/>
		</DAType>
		<DAType id="myVector">
			<BDA name="mag" bType="Struct" type="myAnalogValue"/>
			<BDA name="ang" bType="Struct" type="myAnalogValue"/>
		</DAType>
		<DAType id="xyPoint">
			<BDA name="xVal" bType="FLOAT32"/>
			<BDA name="yVal" bType="FLOAT32"/>
		</DAType>
		<DAType id="myUnit">
			<BDA name="SIUnit" bType="Enum" sAddr="" type="SIUnitKind"/>
			<BDA name="multiplier" bType="Enum" sAddr="" type="MultiplierKind">
				<Val>k</Val>
			</BDA>
		</DAType>
		<DAType id="originator">
			<BDA name="orCat" bType="Enum" type="OriginatorCategoryKind"/>
			<BDA name="orIdent" bType="Octet64"/>
		</DAType>
		<DAType id="myModOper">
			<BDA name="ctlVal" bType="Enum" type="BehaviourModeKind"/>
			<BDA name="origin" bType="Struct" type="originator"/>
			<BDA name="ctlNum" bType="INT8U"/>
			<BDA name="T" bType="Timestamp"/>
			<BDA name="Test" bType="BOOLEAN"/>
			<BDA name="Check" bType="Check"/>
			<ProtNs type="8-MMS">IEC 61850-8-1:2003</ProtNs>
		</DAType>
		<DAType id="myOper">
			<BDA name="ctlVal" bType="BOOLEAN"/>
			<BDA name="origin" bType="Struct" type="originator"/>
			<BDA name="ctlNum" bType="INT8U"/>
			<BDA name="T" bType="Timestamp"/>
			<BDA name="Test" bType="BOOLEAN"/>
			<BDA name="Check" bType="Check"/>
			<ProtNs type="8-MMS">IEC 61850-8-1:2003</ProtNs>
		</DAType>
		<DAType id="myCancel">
			<BDA name="ctlVal" bType="BOOLEAN"/>
			<BDA name="origin" bType="Struct" type="originator"/>
			<BDA name="ctlNum" bType="INT8U"/>
			<BDA name="T" bType="Timestamp"/>
			<BDA name="Test" bType="BOOLEAN"/>
			<ProtNs type="8-MMS">IEC 61850-8-1:2003</ProtNs>
		</DAType>
		<EnumType id="BehaviourModeKind" desc="Behaviour or mode (BehaviourModeKind enumeration)">
			<EnumVal ord="1" desc="Normal enabled state.">on</EnumVal>
			<EnumVal ord="2" desc="Process is passively supervised.">blocked</EnumVal>
			<EnumVal ord="3" desc="Function is operated but results are indicated as test results.">test</EnumVal>
			<EnumVal ord="4" desc="Function is operated in test mode, but with no impact to the
process.">test/blocked</EnumVal>
			<EnumVal ord="5" desc="Function is inactive but shows its configuration capability.">off</EnumVal>
		</EnumType>
		<EnumType id="HealthKind" desc="Health (HealthKind enumeration)">
			<EnumVal ord="1" desc="No problems, normal operation (&quot;green&quot;).">Ok</EnumVal>
			<EnumVal ord="2" desc="Minor problems, but in safe operating mode (&quot;yellow&quot;). The exact meaning is
a local issue, depending on the dedicated function/device.">Warning</EnumVal>
			<EnumVal ord="3" desc="Severe problem, no operation possible (&quot;red&quot;).">Alarm</EnumVal>
		</EnumType>
		<EnumType id="CtlModelKind" desc="Control model (CtlModelKind enumeration)">
			<EnumVal ord="0" desc="The object is not controllable, only the services that apply to a status object are
supported.">status-only</EnumVal>
			<EnumVal ord="1" desc="Direct control with normal security according to IEC 61850-7-2.">direct-with-normalsecurity</EnumVal>
			<EnumVal ord="2" desc="SBO (select before operate) control with normal security according to IEC 61850-7-
2.">sbo-with-normal-security</EnumVal>
			<EnumVal ord="3" desc="Direct control with enhanced security according to IEC 61850-7-2.">direct-withenhanced-
security</EnumVal>
			<EnumVal ord="4" desc="SBO control with enhanced security according to IEC 61850-7-2.">sbo-with-enhancedsecurity</EnumVal>
		</EnumType>
		<EnumType id="FaultDirectionKind" desc="Fault direction (FaultDirectionKind enumeration)">
			<EnumVal ord="0">unknown</EnumVal>
			<EnumVal ord="1">forward</EnumVal>
			<EnumVal ord="2">backward</EnumVal>
			<EnumVal ord="3">both</EnumVal>
		</EnumType>
		<EnumType id="SequenceKind" desc="Sequence (SequenceKind enumeration)">
			<EnumVal ord="0" desc="Sequence components 'CDC.[c1, c2, c3]' are positive, negative and zero,
respectively.">pos-neg-zero</EnumVal>
			<EnumVal ord="1" desc="Sequence components 'CDC.[c1, c2, c3]' are direct, quadratic and zero,
respectively.">dir-quad-zero</EnumVal>
		</EnumType>
		<EnumType id="OriginatorCategoryKind" desc="Originator category (OriginatorCategoryKind enumeration)">
			<EnumVal ord="0" desc="That value shall not be used.">not-supported</EnumVal>
			<EnumVal ord="1" desc="Control operation issued from an operator using a client located at bay level.">baycontrol</EnumVal>
			<EnumVal ord="2" desc="Control operation issued from an operator using a client located at station
level.">station-control</EnumVal>
			<EnumVal ord="3" desc="Control operation from a remote operator outside the substation (for example network
control center).">remote-control</EnumVal>
			<EnumVal ord="4" desc="Control operation issued from an automatic function at bay level.">automaticbay</EnumVal>
			<EnumVal ord="5" desc="Control operation issued from an automatic function at station level.">automaticstation</EnumVal>
			<EnumVal ord="6" desc="Control operation issued from an automatic function outside of the
substation.">automatic-remote</EnumVal>
			<EnumVal ord="7" desc="Control operation issued from a maintenance/service tool.">maintenance</EnumVal>
			<EnumVal ord="8" desc="Status change occurred without control action (for example external trip of a circuit
breaker or failure inside the breaker).">process</EnumVal>
		</EnumType>
		<EnumType id="SIUnitKind" desc="SI unit (SIUnitKind enumeration)">
			<EnumVal ord="1" desc="dimensionless"/>
			<EnumVal ord="2" desc="meter: Length">m</EnumVal>
			<EnumVal ord="3" desc="kilogram: Mass">kg</EnumVal>
			<EnumVal ord="4" desc="second: Time">s</EnumVal>
			<EnumVal ord="5" desc="ampere: Current">A</EnumVal>
			<EnumVal ord="6" desc="kelvin: Temperature">K</EnumVal>
			<EnumVal ord="7" desc="mole: Amount of substance">mol</EnumVal>
			<EnumVal ord="8" desc="candela: Luminous intensity">cd</EnumVal>
			<EnumVal ord="9" desc="degrees: Plane angle">deg</EnumVal>
			<EnumVal ord="10" desc="radian: Plane angle">rad</EnumVal>
			<EnumVal ord="11" desc="steradian: Solid angle">sr</EnumVal>
			<EnumVal ord="21" desc="Gray (J/kg): Absorbed dose">Gy</EnumVal>
			<EnumVal ord="22" desc="becquerel (1/s): Activity">Bq</EnumVal>
			<EnumVal ord="23" desc="degrees Celsius: Relative temperature">°C</EnumVal>
			<EnumVal ord="24" desc="sievert (J/kg): Dose equivalent">Sv</EnumVal>
			<EnumVal ord="25" desc="farad: Electric capacitance">F</EnumVal>
			<EnumVal ord="26" desc="coulomb (A S): Electric charge">C</EnumVal>
			<EnumVal ord="27" desc="siemens (A/V): Electric conductance">S</EnumVal>
			<EnumVal ord="28" desc="henry (Wb/A): Electric inductance">H</EnumVal>
			<EnumVal ord="29" desc="volt (W/A): Electric potential">V</EnumVal>
			<EnumVal ord="30" desc="ohm (V/A): Electric resistance">ohm</EnumVal>
			<EnumVal ord="31" desc="joule (N m): Energy">J</EnumVal>
			<EnumVal ord="32" desc="newton (kg m / s²): Force">N</EnumVal>
			<EnumVal ord="33" desc="hertz (1/s): Frequency">Hz</EnumVal>
			<EnumVal ord="34" desc="lux (Lm/m²): Illuminance">lx</EnumVal>
			<EnumVal ord="35" desc="lumen (cd sr): Luminous flux">Lm</EnumVal>
			<EnumVal ord="36" desc="weber (V s): Magnetic flux">Wb</EnumVal>
			<EnumVal ord="37" desc="tesla (Wb / m²): Magnetic flux density">T</EnumVal>
			<EnumVal ord="38" desc="watt (J/s): Powerwatt (I² R): Real power">W</EnumVal>
			<EnumVal ord="39" desc="pascal (N / m²): Pressure">Pa</EnumVal>
			<EnumVal ord="41" desc="square meter (m²): Area">m²</EnumVal>
			<EnumVal ord="42" desc="cubic meter (m³): Volume">m³</EnumVal>
			<EnumVal ord="43" desc="meters per second (m/s): Velocity">m/s</EnumVal>
			<EnumVal ord="44" desc="meters per second² (m/s²): Acceleration">m/s²</EnumVal>
			<EnumVal ord="45" desc="cubic meters per second (m³/s): Volumetric flow rate">m³/s</EnumVal>
			<EnumVal ord="46" desc="meters/cubic meter (m/m³): Fuel efficiency">m/m³</EnumVal>
			<EnumVal ord="47" desc="kilogram meter (kg m): Moment of mass">M</EnumVal>
			<EnumVal ord="48" desc="kilogram/cubic meter (kg/m³): Density">kg/m³</EnumVal>
			<EnumVal ord="49" desc="meter square/second (m²/s): Viscosity">m²/s</EnumVal>
			<EnumVal ord="50" desc="watt per meter kelvin (W/m K): Thermal conductivity">W/m K</EnumVal>
			<EnumVal ord="51" desc="joule per kelvin (J/K): Heat capacity">J/K</EnumVal>
			<EnumVal ord="52" desc="parts per million: Concentration">ppm</EnumVal>
			<EnumVal ord="53" desc="rotations per second (1/s): Rotational speed">1/s</EnumVal>
			<EnumVal ord="54" desc="radian per second (rad/s): Angular velocity">rad/s</EnumVal>
			<EnumVal ord="55" desc="watt per square meter: Insolation">W/m²</EnumVal>
			<EnumVal ord="56" desc="watt seconds per square meter: Insolation energy">J/m²</EnumVal>
			<EnumVal ord="57" desc="siemens per meter: Electric conductivity">S/m</EnumVal>
			<EnumVal ord="58" desc="kelvin per second: Temperature change rate">K/s</EnumVal>
			<EnumVal ord="59" desc="pascal per second: Pressure change rate">Pa/s</EnumVal>
			<EnumVal ord="60" desc="joule per kilogram per kelvin: Specific heat">J/kg K</EnumVal>
			<EnumVal ord="61" desc="volt ampere (VA): Apparent power">VA</EnumVal>
			<EnumVal ord="62" desc="(Use 'W' instead) watts (I² R): Real power.">Watts</EnumVal>
			<EnumVal ord="63" desc="volt ampere reactive (V I sin(theta)): Reactive power">VAr</EnumVal>
			<EnumVal ord="64" desc="(Use 'deg' or 'rad' instead) degrees: Phase angle.">phi</EnumVal>
			<EnumVal ord="65" desc="dimensionless: Power factor">cos(phi)</EnumVal>
			<EnumVal ord="66" desc="volt seconds (Ws/A): Volt second">Vs</EnumVal>
			<EnumVal ord="67" desc="volt squared (W²/A²): Volt squared">V²</EnumVal>
			<EnumVal ord="68" desc="amp second (As): Amp second">As</EnumVal>
			<EnumVal ord="69" desc="amp square (A²): Amp squared">A²</EnumVal>
			<EnumVal ord="70" desc="amp squared time (A²t): Amps squared time">A²t</EnumVal>
			<EnumVal ord="71" desc="volt ampere hours: Apparent energy">VAh</EnumVal>
			<EnumVal ord="72" desc="watt hours: Real energy">Wh</EnumVal>
			<EnumVal ord="73" desc="volt ampere reactive hours: Reactive energy">VArh</EnumVal>
			<EnumVal ord="74" desc="volts per hertz: Magnetic flux">V/Hz</EnumVal>
			<EnumVal ord="75" desc="hertz per second: Rate of change of frequency">Hz/s</EnumVal>
			<EnumVal ord="76" desc="characters: Number of characters">char</EnumVal>
			<EnumVal ord="77" desc="characters per second: Baud">char/s</EnumVal>
			<EnumVal ord="78" desc="kg square meter: Turbine inertia">kgm²</EnumVal>
			<EnumVal ord="79" desc="decibel: Sound pressure level">dB</EnumVal>
			<EnumVal ord="80" desc="joule per watt-hour: Heat rate">J/Wh</EnumVal>
			<EnumVal ord="81" desc="watt per second: Ramp rate">W/s</EnumVal>
			<EnumVal ord="82" desc="litres per second: Flow rate">l/s</EnumVal>
			<EnumVal ord="83" desc="power measurement relative to 1 mW: Power level">dBm</EnumVal>
			<EnumVal ord="84" desc="hour: Time">h</EnumVal>
			<EnumVal ord="85" desc="minute: Time">min</EnumVal>
		</EnumType>
		<EnumType id="MultiplierKind" desc="Unit multiplier (MultiplierKind enumeration)">
			<EnumVal ord="-24" desc="yocto (10exp(-24))">y</EnumVal>
			<EnumVal ord="-21" desc="zepto (10exp(-21))">z</EnumVal>
			<EnumVal ord="-18" desc="atto (10exp(-18))">a</EnumVal>
			<EnumVal ord="-15" desc="femto (10exp(-15))">f</EnumVal>
			<EnumVal ord="-12" desc="pico (10exp(-12))">p</EnumVal>
			<EnumVal ord="-9" desc="nano (10exp(-9))">n</EnumVal>
			<EnumVal ord="-6" desc="micro (10exp(-6))">µ</EnumVal>
			<EnumVal ord="-3" desc="milli (10exp(-3))">m</EnumVal>
			<EnumVal ord="-2" desc="centi (10exp(-2))">c</EnumVal>
			<EnumVal ord="-1" desc="deci (10exp(-1))">d</EnumVal>
			<EnumVal ord="0" desc="1 (10exp(0))"/>
			<EnumVal ord="1" desc="deca (10exp(1))">da</EnumVal>
			<EnumVal ord="2" desc="hecto (10exp(2))">h</EnumVal>
			<EnumVal ord="3" desc="kilo (10exp(3))">k</EnumVal>
			<EnumVal ord="6" desc="mega (10exp(6))">M</EnumVal>
			<EnumVal ord="9" desc="giga (10exp(9))">G</EnumVal>
			<EnumVal ord="12" desc="tera (10exp(12))">T</EnumVal>
			<EnumVal ord="15" desc="petra (10exp(15))">P</EnumVal>
			<EnumVal ord="18" desc="exa (10exp(18))">E</EnumVal>
			<EnumVal ord="21" desc="zetta (10exp(21))">Z</EnumVal>
			<EnumVal ord="24" desc="yotta (10exp(24))">Y</EnumVal>
		</EnumType>
	</DataTypeTemplates>
</SCL>
